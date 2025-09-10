package morphoroutes

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"
)

func (s *Service) BackgroundJob(job func(ctx map[string]any, service Service) error, period time.Duration, ctx map[string]any) {
	for {
		err := job(ctx, *s)
		if err != nil {
			log.Println(err)
		}
		time.Sleep(period)
	}
}

func RepopulateCSV(ctx map[string]any, service Service) error {
	log.Println("starting csv repopulation job...")
	db, err := service.GetDB()
	if err != nil {
		return NewServerError(err)
	}

	projects, err := GetAllProjects(db)
	if err != nil {
		return NewServerError(err)
	}

	for _, project := range projects {
		_, err := os.Stat(fileUrlGenerator(service, fmt.Sprintf("/assets/%s/data.csv", project.ProjectName)))
		var pathError *fs.PathError
		csvAbsent := errors.As(err, &pathError)
		if csvAbsent {
			log.Printf("writing csv data for %s...\n", project.ProjectName)
			UploadCsv(service, project.ProjectName)
		}
	}

	return nil
}

func fileUrlGenerator(service Service, filename string) string {
	switch service.ENVIRONMENT {
	case "prod":
		return fmt.Sprintf("%s/%s", service.S3_MOUNTPOINT, filename)
	case "dev":
		return fmt.Sprintf("./%s", filename)
	default:
		return ""
	}
}

func RepopulateArchiveZip(ctx map[string]any, service Service) error {
	log.Println("starting archival job...")
	db, err := service.GetDB()
	if err != nil {
		return NewServerError(err)
	}

	projects, err := GetAllProjects(db)
	if err != nil {
		return NewServerError(err)
	}

	for _, project := range projects {
		_, err := os.Stat(fileUrlGenerator(service, fmt.Sprintf("assets/%s/archive.zip", project.ProjectName)))
		var pathError *fs.PathError
		archiveAbsent := errors.As(err, &pathError)
		if archiveAbsent {
			err = UploadArchive(service, project.ProjectName)
			if err != nil {
				log.Println(err)
			}
			log.Printf("wrote archive for %s...\n", project.ProjectName)
		}
	}

	return nil
}
